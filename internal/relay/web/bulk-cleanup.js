'use strict';
window.cleanupItems = async (items, remove) => {
  const results=[];
  for(const item of items) {
    try {await remove(item);results.push({item,deleted:true});}
    catch(error){results.push({item,deleted:false,error:error.message});}
  }
  return results;
};
