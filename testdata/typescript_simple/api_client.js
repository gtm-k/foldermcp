const axios = require('axios');
const _ = require('lodash');

/**
 * Fetch user data from the API.
 * @param {string} userId - The user ID
 * @returns {Promise<Object>} User data
 */
module.exports.fetchUser = async function(userId) {
  const response = await axios.get(`/api/users/${userId}`);
  return response.data;
};
